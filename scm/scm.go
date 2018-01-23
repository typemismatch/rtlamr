// RTLAMR - An rtl-sdr receiver for smart meters operating in the 900MHz ISM band.
// Copyright (C) 2015 Douglas Hall
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package scm

import (
	"encoding/binary"
	"fmt"
	"strconv"

	"crypto/tls"
	"crypto/x509"
	"github.com/bemasher/rtlamr/crc"
	"github.com/bemasher/rtlamr/decode"
	"github.com/bemasher/rtlamr/parse"
	"io/ioutil"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"
)

func init() {
	parse.Register("scm", NewParser)
}

// NewTLSConfig Setup the TLS configuration
func NewTLSConfig() *tls.Config {
	// Import trusted certificates from CAfile.pem.
	// Alternatively, manually add CA certificates to
	// default openssl CA bundle.
	certpool := x509.NewCertPool()
	pemCerts, err := ioutil.ReadFile("rootCA.pem")
	if err == nil {
		certpool.AppendCertsFromPEM(pemCerts)
	}

	// Import client certificate/key pair
	cert, err := tls.LoadX509KeyPair("rtlsdr.certificate.crt", "rtlsdr.private.key")
	if err != nil {
		panic(err)
	}

	// Just to print out the client certificate..
	cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		panic(err)
	}
	fmt.Println(cert.Leaf)

	// Create tls.Config with desired tls properties
	return &tls.Config{
		// RootCAs = certs used to verify server cert.
		RootCAs: certpool,
		// ClientAuth = whether to request cert from server.
		// Since the server is set up for SSL, this happens
		// anyways.
		ClientAuth: tls.NoClientCert,
		// ClientCAs = certs used to validate client cert.
		ClientCAs: nil,
		// InsecureSkipVerify = verify that cert contents
		// match server. IP matches what is in cert etc.
		InsecureSkipVerify: true,
		// Certificates = list of certs client sends to server.
		Certificates: []tls.Certificate{cert},
	}
}

func NewPacketConfig(chipLength int) (cfg decode.PacketConfig) {
	cfg.CenterFreq = 912600155
	cfg.DataRate = 32768
	cfg.ChipLength = chipLength
	cfg.PreambleSymbols = 21
	cfg.PacketSymbols = 96
	cfg.Preamble = "111110010101001100000"

	return
}

type Parser struct {
	decode.Decoder
	crc.CRC
}

func NewParser(chipLength int) (p parse.Parser) {
	return &Parser{
		decode.NewDecoder(NewPacketConfig(chipLength)),
		crc.NewCRC("BCH", 0, 0x6F63, 0),
	}
}

func (p Parser) Dec() decode.Decoder {
	return p.Decoder
}

func (p *Parser) Cfg() *decode.PacketConfig {
	return &p.Decoder.Cfg
}

func (p Parser) Parse(indices []int) (msgs []parse.Message) {
	seen := make(map[string]bool)

	for _, pkt := range p.Decoder.Slice(indices) {
		s := string(pkt)
		if seen[s] {
			continue
		}
		seen[s] = true

		data := parse.NewDataFromBytes(pkt)

		// If the packet is too short, bail.
		if l := len(data.Bytes); l != 12 {
			continue
		}

		// If the checksum fails, bail.
		if p.Checksum(data.Bytes[2:12]) != 0 {
			continue
		}

		scm := NewSCM(data)

		// If the meter id is 0, bail.
		if scm.ID == 0 {
			continue
		}

		msgs = append(msgs, scm)
	}

	return
}

// Standard Consumption Message
type SCM struct {
	ID          uint32 `xml:",attr"`
	Type        uint8  `xml:",attr"`
	TamperPhy   uint8  `xml:",attr"`
	TamperEnc   uint8  `xml:",attr"`
	Consumption uint32 `xml:",attr"`
	ChecksumVal uint16 `xml:"Checksum,attr"`
}

func NewSCM(data parse.Data) (scm SCM) {
	ertid, _ := strconv.ParseUint(data.Bits[21:23]+data.Bits[56:80], 2, 26)
	erttype, _ := strconv.ParseUint(data.Bits[26:30], 2, 4)
	tamperphy, _ := strconv.ParseUint(data.Bits[24:26], 2, 2)
	tamperenc, _ := strconv.ParseUint(data.Bits[30:32], 2, 2)
	consumption, _ := strconv.ParseUint(data.Bits[32:56], 2, 24)
	checksum, _ := strconv.ParseUint(data.Bits[80:96], 2, 16)

	scm.ID = uint32(ertid)
	scm.Type = uint8(erttype)
	scm.TamperPhy = uint8(tamperphy)
	scm.TamperEnc = uint8(tamperenc)
	scm.Consumption = uint32(consumption)
	scm.ChecksumVal = uint16(checksum)

	return
}

func (scm SCM) MsgType() string {
	return "SCM"
}

func (scm SCM) MeterID() uint32 {
	return scm.ID
}

func (scm SCM) MeterType() uint8 {
	return scm.Type
}

func (scm SCM) Checksum() []byte {
	checksum := make([]byte, 2)
	binary.BigEndian.PutUint16(checksum, scm.ChecksumVal)
	return checksum
}

func (scm SCM) String() string {
	// Setup the broker connection
	tlsconfig := NewTLSConfig()

	opts := MQTT.NewClientOptions()
	opts.AddBroker("ssl://data.iot.us-west-2.amazonaws.com:8883")
	opts.SetClientID("rtlsdr").SetTLSConfig(tlsconfig)
	c := MQTT.NewClient(opts)
	if token := c.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}
	// write this message out to AWS IoT
	c.Publish("/rtlsdr", 0, false, fmt.Sprintf("{ID:%8d Type:%2d Tamper:{Phy:%02X Enc:%02X} Consumption:%8d CRC:0x%04X}",
		scm.ID, scm.Type, scm.TamperPhy, scm.TamperEnc, scm.Consumption, scm.ChecksumVal,
	))
	c.Disconnect(250)
	return fmt.Sprintf("{ID:%8d Type:%2d Tamper:{Phy:%02X Enc:%02X} Consumption:%8d CRC:0x%04X}",
		scm.ID, scm.Type, scm.TamperPhy, scm.TamperEnc, scm.Consumption, scm.ChecksumVal,
	)
}

func (scm SCM) Record() (r []string) {
	r = append(r, strconv.FormatUint(uint64(scm.ID), 10))
	r = append(r, strconv.FormatUint(uint64(scm.Type), 10))
	r = append(r, "0x"+strconv.FormatUint(uint64(scm.TamperPhy), 16))
	r = append(r, "0x"+strconv.FormatUint(uint64(scm.TamperEnc), 16))
	r = append(r, strconv.FormatUint(uint64(scm.Consumption), 10))
	r = append(r, "0x"+strconv.FormatUint(uint64(scm.ChecksumVal), 16))

	return
}
